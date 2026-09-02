class Base {};
class Main inherits Base {
  value : Int;
  initialized : Int <- 1;
  zero() : Int { 0 };
  one(x : Int) : Int { x };
  many(x : Int, y : Int, z : Int) : Int { x + y * z };
  main() : Object {
    {
      value <- many(1, 2, 3);
      self@Main.one(value);
      (new Main).zero();
      if not false then value else 0 fi;
      while false loop value <- value + 1 pool;
      let item : Base <- new Main, other : Int in isvoid item;
      case self of
        main : Main => main;
        base : Base => base;
      esac;
      new SELF_TYPE;
    }
  };
};
